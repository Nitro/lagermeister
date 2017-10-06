import { Component } from '@angular/core';
import * as _ from "lodash";

// Services
import { ChartsService } from '../charts.service';

@Component({
    selector: 'lag-chart',
    templateUrl: './lag-chart.component.html',
    providers: [ChartsService],
})

export class LagChartComponent {

    lagChartObject: any;

    constructor(private chartsService: ChartsService) {

        this.lagChartObject = {
            chartType: 'LineChart',
            data: {},
            chart: null,
            domElement: 'lag-chart',
            columns: [['Timestamp', 'datetime'], ['Lag (secs)', 'number']],
            options: Object.assign({
                width: 1000,
                vAxis: {
                    title: 'Lag (secs)',
                },
                hAxis: {
                    viewWindow: {}
                },
                colors: ['#E24500'],
            }, this.chartsService.getLineChartOptions())
        };

        let script = document.createElement('script');
        script.src = '//www.google.com/jsapi';
        script.onload = () => {
            (<any>window).google.charts.load("visualization", "1", {packages:["corechart", "gauge"]});
            (<any>window).google.charts.setOnLoadCallback(this.chartsService.drawLineChart.bind(this, this.lagChartObject));
        };
        document.head.appendChild(script);

        let pendingData: Array<Object> = [];

        let obsv = this.chartsService.getObservable();
        obsv.subscribe( (evt: any)  => {
            let data = JSON.parse(evt.data);
            if (data.MetricType === 'Lag') {
                pendingData.push(data);
            }
        });

        setInterval(() => {
            _.forEach(pendingData, (data: any) => {
                if(this.lagChartObject.chartReady) {
                    this.chartsService.updateLineChart(data, this.lagChartObject);
                }
            });
            pendingData.splice(0);
        }, 1000)

    }
}
